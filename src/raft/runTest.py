import os

num_tests = 50


if __name__ == "__main__":
    for i in range(num_tests):
        print("ROUND " + str(i))

        # with log
        file_name = "out" + str(i)
        os.system("go test -run 2C >" + file_name)
        with open(file_name) as f:
            if 'FAIL' in f.read():
                print(file_name + " fails")
                continue
            else:
                print(file_name + " ok")
        os.system("rm " + file_name)


        # without log
        # os.system("go test -run 2B")
